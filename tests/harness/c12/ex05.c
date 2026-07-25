#include "ft_list.h"
#include <unistd.h>

t_list	*ft_list_push_strs(int size, char **strs);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

int	main(int argc, char **argv)
{
	t_list	*begin;

	begin = ft_list_push_strs(argc - 1, argv + 1);
	print_list_str(begin);
	return (0);
}
