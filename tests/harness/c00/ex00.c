void	ft_putchar(char c);

int	main(int argc, char **argv)
{
	if (argc < 2 || argv[1][0] == '\0')
		return (1);
	ft_putchar(argv[1][0]);
	return (0);
}
