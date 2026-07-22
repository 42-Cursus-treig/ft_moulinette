#include <unistd.h>

void	ft_advanced_sort_string_tab(char **tab, int(*cmp)(char *, char *));

static int	ft_strcmp(char *a, char *b)
{
	int	i;

	i = 0;
	while (a[i] && a[i] == b[i])
		i++;
	return ((unsigned char)a[i] - (unsigned char)b[i]);
}

static int	rev_strcmp(char *a, char *b)
{
	return (-ft_strcmp(a, b));
}

static int	len_cmp(char *a, char *b)
{
	int	i;
	int	j;

	i = 0;
	while (a[i])
		i++;
	j = 0;
	while (b[j])
		j++;
	return (i - j);
}

static void	putstr_nl(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
	write(1, "\n", 1);
}

int	main(int argc, char **argv)
{
	int	(*f)(char *, char *);
	int	i;

	if (argc < 2)
		return (0);
	f = &ft_strcmp;
	if (argv[1][0] == 'd')
		f = &rev_strcmp;
	else if (argv[1][0] == 'l')
		f = &len_cmp;
	ft_advanced_sort_string_tab(argv + 2, f);
	i = 2;
	while (argv[i])
	{
		putstr_nl(argv[i]);
		i++;
	}
	return (0);
}
